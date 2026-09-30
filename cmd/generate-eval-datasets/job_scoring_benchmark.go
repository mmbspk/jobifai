package main

import "fmt"

func jobScoringFullBenchmarkCases() []map[string]any {
	all := append(jobScoringScenarios(), jobScoringExtendedScenarios()...)
	out := make([]map[string]any, 0, len(all))
	for i, s := range all {
		id := fmt.Sprintf("job_scoring-%04d", i+1)
		exp := map[string]any{"pass_threshold": 7, "scenario": s.Tag}
		switch {
		case s.ExpectBorderline:
			exp["expect_borderline"] = true
		case s.ExpectPass:
			exp["expect_pass"] = true
			exp["min_score"] = 7
		default:
			exp["expect_skip"] = true
			exp["max_score"] = 4
		}
		out = append(out, map[string]any{
			"id": id, "task": "job_scoring", "classification": "synthetic", "critical": s.Critical,
			"tags": []string{s.Tag, s.Profession},
			"input": map[string]any{"profile": s.Profile, "job_description": s.JobDesc},
			"expect": exp,
		})
	}
	return out
}

func jobScoringExtendedScenarios() []jobScenario {
	return []jobScenario{
		{Tag: "backend_strong", Profession: "software", ExpectPass: true, Critical: true,
			Profile: techProfile("Backend Engineer", "2017", []string{"Go", "PostgreSQL", "REST APIs"}),
			JobDesc: "Backend role building Go services and PostgreSQL data stores for customer platforms."},
		{Tag: "frontend_strong", Profession: "software", ExpectPass: true, Critical: true,
			Profile: techProfile("Frontend Engineer", "2018", []string{"React", "TypeScript", "accessibility"}),
			JobDesc: "Frontend engineer for React and TypeScript product UI with accessibility standards."},
		{Tag: "devops_strong", Profession: "software", ExpectPass: true, Critical: true,
			Profile: techProfile("DevOps Engineer", "2016", []string{"Kubernetes", "Terraform", "CI/CD"}),
			JobDesc: "DevOps engineer to manage Kubernetes clusters and Terraform infrastructure pipelines."},
		{Tag: "data_engineer_strong", Profession: "software", ExpectPass: true, Critical: true,
			Profile: techProfile("Data Engineer", "2017", []string{"Spark", "dbt", "SQL"}),
			JobDesc: "Data engineer building batch pipelines with Spark, dbt, and warehouse SQL."},
		{Tag: "cyber_strong", Profession: "software", ExpectPass: true, Critical: true,
			Profile: techProfile("Security Analyst", "2018", []string{"SIEM", "incident response", "vulnerability scanning"}),
			JobDesc: "Cybersecurity analyst for SIEM monitoring and incident response in enterprise environments."},
		{Tag: "qa_strong", Profession: "software", ExpectPass: true, Critical: false,
			Profile: techProfile("QA Engineer", "2019", []string{"test automation", "Selenium", "regression testing"}),
			JobDesc: "QA engineer owning automated regression suites with Selenium and CI integration."},
		{Tag: "backend_missing_go", Profession: "software", ExpectPass: false, Critical: true,
			Profile: techProfile("Java Developer", "2015", []string{"Java", "Spring"}),
			JobDesc: "Senior Go engineer mandatory; daily Go microservice development required."},
		{Tag: "enrolled_nurse_aged_care", Profession: "healthcare", ExpectPass: true, Critical: false,
			Profile: nurseProfile("Enrolled Nurse", "2017", []string{"aged care", "medication rounds"}),
			JobDesc: "Enrolled nurse for aged-care facility medication rounds and personal care support."},
		{Tag: "hca_junior_ward", Profession: "healthcare", ExpectPass: true, Critical: false,
			Profile: nurseProfile("Healthcare Assistant", "2020", []string{"patient transport", "vitals"}),
			JobDesc: "Healthcare assistant supporting ward teams with vitals and patient transport."},
		{Tag: "clinical_coordinator_fit", Profession: "healthcare", ExpectPass: true, Critical: true,
			Profile: nurseProfile("Clinical Coordinator", "2013", []string{"ward rostering", "staff coordination"}),
			JobDesc: "Clinical coordinator for surgical ward rostering and daily staff coordination."},
		{Tag: "accountant_audit", Profession: "business", ExpectPass: true, Critical: true,
			Profile: businessProfile("Accountant", "2014", []string{"financial reporting", "audit support"}),
			JobDesc: "Accountant preparing statutory financial reports and external audit schedules."},
		{Tag: "pm_construction", Profession: "business", ExpectPass: true, Critical: true,
			Profile: pmProfile("Project Manager", "2012", []string{"subcontractor management", "commercial builds"}),
			JobDesc: "Construction project manager for commercial builds and subcontractor delivery."},
		{Tag: "product_saas", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("Product Manager", "2016", []string{"roadmapping", "user research", "SaaS"}),
			JobDesc: "Product manager for B2B SaaS roadmap and discovery with enterprise clients."},
		{Tag: "hr_generalist", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("HR Specialist", "2015", []string{"employee relations", "policy"}),
			JobDesc: "HR generalist handling employee relations and workplace policy implementation."},
		{Tag: "marketing_digital", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("Marketing Manager", "2014", []string{"paid social", "campaign analytics"}),
			JobDesc: "Digital marketing manager running paid social campaigns and performance analytics."},
		{Tag: "sales_b2b", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("Sales Executive", "2013", []string{"enterprise sales", "pipeline management"}),
			JobDesc: "B2B sales executive managing enterprise pipeline and contract negotiations."},
		{Tag: "mechanical_design", Profession: "engineering", ExpectPass: true, Critical: true,
			Profile: tradeProfile("Mechanical Engineer", "2011", []string{"CAD", "HVAC design"}),
			JobDesc: "Mechanical engineer for HVAC design using CAD tools in commercial buildings."},
		{Tag: "electrician_licensed", Profession: "engineering", ExpectPass: true, Critical: true,
			Profile: tradeProfile("Electrician", "2010", []string{"electrical maintenance", "switchboards"}),
			JobDesc: "Licensed electrician for switchboard maintenance and commercial electrical installs."},
		{Tag: "construction_site_mgr", Profession: "engineering", ExpectPass: true, Critical: true,
			Profile: pmProfile("Construction Manager", "2009", []string{"site safety", "trade coordination"}),
			JobDesc: "Construction manager overseeing onsite safety and trade coordination for multi-storey builds."},
		{Tag: "office_admin", Profession: "general", ExpectPass: true, Critical: false,
			Profile: businessProfile("Office Administrator", "2018", []string{"scheduling", "invoicing", "MS Office"}),
			JobDesc: "Office administrator for scheduling, invoicing, and front-desk operations."},
		{Tag: "graduate_analyst", Profession: "general", ExpectPass: true, Critical: false,
			Profile: businessProfile("Graduate Analyst", "2024", []string{"research", "Excel", "presentations"}),
			JobDesc: "Graduate analyst program for recent graduates with strong Excel and presentation skills."},
		{Tag: "hospitality_chef", Profession: "general", ExpectPass: true, Critical: false,
			Profile: tradeProfile("Chef", "2012", []string{"menu planning", "kitchen leadership", "food safety"}),
			JobDesc: "Head chef leading kitchen team, menu planning, and food safety compliance."},
		{Tag: "remote_only_mismatch", Profession: "software", ExpectPass: false, Critical: false,
			Profile: techProfile("Support Engineer", "2016", []string{"customer support", "ticketing"}),
			JobDesc: "Onsite-only role in Brisbane office; remote work is not available."},
		{Tag: "certification_required", Profession: "healthcare", ExpectPass: false, Critical: true,
			Profile: nurseProfile("General Nurse", "2014", []string{"ward care"}),
			JobDesc: "Registered nurse role requiring current ACLS certification and recent critical-care experience."},
		{Tag: "adjacent_tech_fit", Profession: "software", ExpectPass: false, Critical: false,
			Profile: techProfile("Platform Engineer", "2017", []string{"Docker", "Linux", "monitoring"}),
			JobDesc: "Kubernetes platform engineer; container orchestration and observability experience required."},
		{Tag: "title_mismatch_skill_fit", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("Client Success Lead", "2015", []string{"account management", "renewals"}),
			JobDesc: "Account manager for enterprise renewals and expansion conversations."},
		{Tag: "accountant_tax_mismatch", Profession: "business", ExpectPass: false, Critical: true,
			Profile: businessProfile("Management Accountant", "2013", []string{"budgeting", "FP&A"}),
			JobDesc: "Tax accountant specializing in corporate tax returns and ATO lodgements only."},
		{Tag: "chef_retail_mismatch", Profession: "general", ExpectPass: false, Critical: false,
			Profile: tradeProfile("Pastry Chef", "2014", []string{"patisserie", "dessert menus"}),
			JobDesc: "Fast-food grill cook role; short-order cooking experience required."},
		{Tag: "data_analyst_bi", Profession: "business", ExpectPass: true, Critical: false,
			Profile: businessProfile("Data Analyst", "2017", []string{"Power BI", "SQL", "dashboards"}),
			JobDesc: "Business intelligence analyst building Power BI dashboards from SQL sources."},
		{Tag: "cyber_missing_siem", Profession: "software", ExpectPass: false, Critical: true,
			Profile: techProfile("IT Support", "2016", []string{"helpdesk", "Active Directory"}),
			JobDesc: "Security operations role requiring hands-on SIEM tuning and threat hunting."},
		{Tag: "nurse_remote_telehealth", Profession: "healthcare", ExpectPass: true, Critical: false,
			Profile: nurseProfileWithLocation("Registered Nurse", "2016", []string{"telehealth", "triage"}, "Sydney"),
			JobDesc: "Telehealth nurse conducting remote triage for patients across New South Wales."},
		{Tag: "electrician_unlicensed", Profession: "engineering", ExpectPass: false, Critical: true,
			Profile: tradeProfile("Electrical Apprentice", "2023", []string{"wiring assistance"}),
			JobDesc: "Unrestricted electrical licence mandatory for commercial maintenance work."},
		{Tag: "pm_agile_cert", Profession: "business", ExpectPass: false, Critical: true,
			Profile: pmProfile("Coordinator", "2019", []string{"scheduling"}),
			JobDesc: "Agile project manager with PMP and Scrum Master certification required."},
		{Tag: "sales_retail_to_enterprise", Profession: "business", ExpectPass: false, Critical: false,
			Profile: businessProfile("Retail Sales", "2018", []string{"POS", "customer service"}),
			JobDesc: "Enterprise SaaS sales director managing six-figure annual contracts."},
		{Tag: "graduate_overqualified", Profession: "general", ExpectPass: false, Critical: false,
			Profile: businessProfile("Operations Director", "2008", []string{"P&L ownership", "executive leadership"}),
			JobDesc: "Entry-level graduate trainee supporting basic reporting tasks."},
		{Tag: "hr_no_employee_relations", Profession: "business", ExpectPass: false, Critical: true,
			Profile: businessProfile("Recruiter", "2017", []string{"sourcing", "interviewing"}),
			JobDesc: "HR business partner leading complex employee relations investigations."},
		{Tag: "mechanical_to_civil", Profession: "engineering", ExpectPass: false, Critical: true,
			Profile: tradeProfile("Mechanical Engineer", "2012", []string{"HVAC", "CAD"}),
			JobDesc: "Civil engineer for road bridge structural design and geotechnical reports."},
		{Tag: "qa_to_dev_mismatch", Profession: "software", ExpectPass: false, Critical: true,
			Profile: techProfile("QA Engineer", "2016", []string{"manual testing"}),
			JobDesc: "Senior backend developer leading architecture for payment services."},
		{Tag: "health_insufficient_years", Profession: "healthcare", ExpectPass: false, Critical: true,
			Profile: nurseProfile("Graduate Nurse", "2024", []string{"clinical placement"}),
			JobDesc: "Ward nurse with minimum five years post-registration acute experience."},
		{Tag: "construction_foreman", Profession: "engineering", ExpectPass: true, Critical: true,
			Profile: tradeProfile("Site Foreman", "2011", []string{"civil works", "crew leadership"}),
			JobDesc: "Site foreman leading civil works crews and daily progress reporting."},
		{Tag: "marketing_brand_not_performance", Profession: "business", ExpectPass: false, Critical: false,
			Profile: businessProfile("Brand Manager", "2015", []string{"brand strategy", "creative campaigns"}),
			JobDesc: "Performance marketing specialist for paid search ROAS optimization."},
		{Tag: "admin_to_paralegal", Profession: "general", ExpectPass: false, Critical: true,
			Profile: businessProfile("Office Administrator", "2016", []string{"scheduling", "invoicing"}),
			JobDesc: "Paralegal supporting litigation discovery and court filing preparation."},
		{Tag: "data_engineer_to_analyst", Profession: "software", ExpectPass: false, Critical: false,
			Profile: techProfile("Data Engineer", "2017", []string{"Spark", "Airflow"}),
			JobDesc: "Junior marketing analyst creating weekly campaign spreadsheets only."},
		{Tag: "chef_hospitality_strong", Profession: "general", ExpectPass: true, Critical: false,
			Profile: tradeProfile("Sous Chef", "2013", []string{"kitchen operations", "HACCP"}),
			JobDesc: "Sous chef for busy hotel kitchen with HACCP and team leadership duties."},
		{Tag: "onsite_location_fit", Profession: "healthcare", ExpectPass: true, Critical: false,
			Profile: nurseProfileWithLocation("Registered Nurse", "2014", []string{"community care"}, "Melbourne"),
			JobDesc: "Community nurse role based in Melbourne visiting clients across the metro area."},
	}
}

func techProfile(title, since string, skills []string) map[string]any {
	return map[string]any{
		"skills": skills,
		"experience_details": []map[string]any{{
			"position": title, "company": "TechWorks", "employment_period": since + " – Present",
		}},
	}
}

func businessProfile(title, since string, skills []string) map[string]any {
	return map[string]any{
		"skills": skills,
		"experience_details": []map[string]any{{
			"position": title, "company": "Harbor & Co", "employment_period": since + " – Present",
		}},
	}
}

func tradeProfile(title, since string, skills []string) map[string]any {
	return map[string]any{
		"skills": skills,
		"experience_details": []map[string]any{{
			"position": title, "company": "Field Services Group", "employment_period": since + " – Present",
		}},
	}
}
